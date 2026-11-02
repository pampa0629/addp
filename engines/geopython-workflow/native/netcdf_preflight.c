/* Metadata-only admission for the bounded NetCDF-4 root numeric profile.
 * Values and public attributes are read exclusively by GDAL after this check.
 * Use the stable HDF5 1.10 API surface on all supported native libraries. */
#define H5_USE_110_API
#include <hdf5.h>
#include <netcdf.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>

#define MAX_OBJECTS 512
#define MAX_RANK 8
#define MAX_ATTRS 256
#define MAX_ELEMENTS 4096
#define MAX_ATTR_BYTES 65536
#define MAX_CHUNK_BYTES (16 * 1024 * 1024)
#define REQUIRE(c,m) do { if (!(c)) return reject(m); } while (0)

typedef struct {
    char name[NC_MAX_NAME + 1];
    haddr_t address;
    int rank, type, scale, variable;
    hsize_t shape[MAX_RANK];
    int targets[MAX_RANK];
    unsigned char backrefs[MAX_OBJECTS][MAX_RANK];
} Object;

typedef struct { hobj_ref_t dataset; int64_t dimension; } Backref;
static Object objects[MAX_OBJECTS];
static int count;
static unsigned long file_number;
static const char *failure;
static size_t attribute_bytes;

static int reject(const char *message) { if (!failure) failure = message; return -1; }

static int numeric_type(hid_t t) {
    const hid_t types[] = {H5T_STD_I8LE,H5T_STD_I8BE,H5T_STD_U8LE,H5T_STD_U8BE,
        H5T_STD_I16LE,H5T_STD_I16BE,H5T_STD_U16LE,H5T_STD_U16BE,
        H5T_STD_I32LE,H5T_STD_I32BE,H5T_STD_U32LE,H5T_STD_U32BE,
        H5T_STD_I64LE,H5T_STD_I64BE,H5T_STD_U64LE,H5T_STD_U64BE,
        H5T_IEEE_F32LE,H5T_IEEE_F32BE,H5T_IEEE_F64LE,H5T_IEEE_F64BE};
    const int nc[] = {NC_BYTE,NC_BYTE,NC_UBYTE,NC_UBYTE,NC_SHORT,NC_SHORT,
        NC_USHORT,NC_USHORT,NC_INT,NC_INT,NC_UINT,NC_UINT,NC_INT64,NC_INT64,
        NC_UINT64,NC_UINT64,NC_FLOAT,NC_FLOAT,NC_DOUBLE,NC_DOUBLE};
    for (size_t i=0;i<sizeof(nc)/sizeof(nc[0]);i++) if (H5Tequal(t,types[i])>0) return nc[i];
    return 0;
}

static int text_type(hid_t t) {
    return H5Tget_class(t)==H5T_STRING && H5Tis_variable_str(t)==0 &&
        H5Tget_size(t)>0 && H5Tget_size(t)<=MAX_ATTR_BYTES;
}

static int attribute_shape(hid_t a, int *rank, hssize_t *n) {
    hid_t space=H5Aget_space(a); REQUIRE(space>=0,"invalid attribute dataspace");
    *rank=H5Sget_simple_extent_ndims(space); *n=H5Sget_simple_extent_npoints(space);
    H5Sclose(space);
    REQUIRE(*rank>=0 && *rank<=MAX_RANK && *n>0 && *n<=MAX_ELEMENTS,"attribute shape exceeds profile");
    return 0;
}

static herr_t attribute_cb(hid_t owner, const char *name, const H5A_info_t *unused, void *context) {
    (void)unused; Object *o=context;
    REQUIRE(strlen(name)<=NC_MAX_NAME,"attribute name exceeds profile");
    hid_t a=H5Aopen(owner,name,H5P_DEFAULT), t=H5Aget_type(a);
    REQUIRE(a>=0 && t>=0,"attribute type unavailable");
    int rank; hssize_t n;
    REQUIRE(attribute_shape(a,&rank,&n)==0,"attribute shape unavailable");
    hsize_t stored=H5Aget_storage_size(a);
    REQUIRE(H5Tget_size(t)<=MAX_ATTR_BYTES/(size_t)n && stored<=MAX_ATTR_BYTES-attribute_bytes,
        "attribute storage exceeds profile");
    attribute_bytes+=(size_t)stored;
    if (o && !strcmp(name,"DIMENSION_LIST")) {
        REQUIRE(rank==1 && n==o->rank && H5Tget_class(t)==H5T_VLEN,"invalid dimension reference declaration");
        hid_t base=H5Tget_super(t); REQUIRE(base>=0,"dimension reference base unavailable");
        int valid=H5Tequal(base,H5T_STD_REF_OBJ)>0; H5Tclose(base);
        REQUIRE(valid,"unsupported dimension reference type");
    } else if (o && !strcmp(name,"REFERENCE_LIST")) {
        REQUIRE(rank==1 && H5Tget_class(t)==H5T_COMPOUND && H5Tget_nmembers(t)==2,
            "invalid reverse dimension references");
        REQUIRE(H5Tget_member_index(t,"dataset")==0 && H5Tget_member_index(t,"dimension")==1,
            "invalid reverse reference members");
        hid_t ref=H5Tget_member_type(t,0), axis=H5Tget_member_type(t,1);
        int axis_type=axis>=0 ? numeric_type(axis) : 0;
        int valid=ref>=0 && H5Tequal(ref,H5T_STD_REF_OBJ)>0 && (axis_type==NC_INT || axis_type==NC_UINT);
        if(ref>=0)H5Tclose(ref);
        if(axis>=0)H5Tclose(axis);
        REQUIRE(valid,"unsupported reverse reference types");
    } else {
        REQUIRE(H5Tdetect_class(t,H5T_REFERENCE)==0 && (numeric_type(t)||text_type(t)),
            "unsupported attribute type or reference");
        REQUIRE(strcmp(name,"external_variables"),"external NetCDF variables are unsupported");
    }
    H5Tclose(t);H5Aclose(a);return 0;
}

static int attributes(hid_t owner, Object *o) {
    H5O_info_t info; REQUIRE(H5Oget_info(owner,&info)>=0 && info.num_attrs<=MAX_ATTRS,"attribute count exceeds profile");
    attribute_bytes=0;
    return H5Aiterate2(owner,H5_INDEX_NAME,H5_ITER_INC,NULL,attribute_cb,o)<0 ? reject("attribute inspection failed") : 0;
}

static int read_text(hid_t owner,const char *name,char *buffer,size_t capacity) {
    hid_t a=H5Aopen(owner,name,H5P_DEFAULT),t=H5Aget_type(a);
    REQUIRE(a>=0 && t>=0 && text_type(t),"required text declaration unavailable");
    int rank;hssize_t n;REQUIRE(attribute_shape(a,&rank,&n)==0 && rank==0 && n==1,"required text must be scalar");
    size_t size=H5Tget_size(t);REQUIRE(size<capacity,"required text exceeds profile");
    memset(buffer,0,capacity);REQUIRE(H5Aread(a,t,buffer)>=0,"required text read failed");
    H5Tclose(t);H5Aclose(a);return 0;
}

static int layout(hid_t d,size_t size,int rank) {
    hid_t p=H5Dget_create_plist(d);REQUIRE(p>=0,"dataset layout unavailable");
    H5D_layout_t form=H5Pget_layout(p);
    REQUIRE((form==H5D_CONTIGUOUS || form==H5D_CHUNKED) && H5Pget_external_count(p)==0,
        "external, virtual or unsupported storage layout");
    if(form==H5D_CHUNKED) {
        hsize_t chunk[MAX_RANK]; REQUIRE(rank>0 && H5Pget_chunk(p,MAX_RANK,chunk)==rank,"invalid chunk dimensions");
        size_t bytes=size;
        for(int i=0;i<rank;i++) { REQUIRE(chunk[i]>0 && chunk[i]<=MAX_CHUNK_BYTES/bytes,"decoded chunk exceeds profile");bytes*=chunk[i]; }
    }
    int filters=H5Pget_nfilters(p);REQUIRE(filters>=0 && filters<=3,"filter count exceeds profile");
    unsigned seen=0;
    for(int i=0;i<filters;i++) {
        unsigned flags=0,values[16];size_t n=16;
        H5Z_filter_t id=H5Pget_filter2(p,i,&flags,&n,values,0,NULL,NULL);
        REQUIRE(id>=1 && id<=3 && !(seen&(1u<<id)) && !(flags&~H5Z_FLAG_OPTIONAL),"unsupported or repeated filter");
        REQUIRE((id==H5Z_FILTER_DEFLATE && n==1 && values[0]<=9) ||
            (id==H5Z_FILTER_SHUFFLE && n==1 && values[0]==size) ||
            (id==H5Z_FILTER_FLETCHER32 && n==0),"invalid filter parameters");
        seen|=1u<<id;
    }
    H5Pclose(p);return 0;
}

static herr_t collect(hid_t root,const char *name,const H5L_info_t *link,void *unused) {
    (void)unused;
    REQUIRE(link->type==H5L_TYPE_HARD,"non-hard links are unsupported");
    REQUIRE(count<MAX_OBJECTS && strlen(name)>0 && strlen(name)<=NC_MAX_NAME,"object index exceeds profile");
    H5O_info_t info;REQUIRE(H5Oget_info_by_name(root,name,&info,H5P_DEFAULT)>=0,"object identity unavailable");
    REQUIRE(info.type==H5O_TYPE_DATASET && info.rc==1 && info.fileno==file_number,"groups, aliases or unsupported objects");
    Object *o=&objects[count++];strcpy(o->name,name);o->address=info.addr;o->variable=-1;
    for(int i=0;i<MAX_RANK;i++)o->targets[i]=-1;
    hid_t d=H5Dopen2(root,name,H5P_DEFAULT),t=H5Dget_type(d),s=H5Dget_space(d);
    REQUIRE(d>=0 && t>=0 && s>=0,"dataset metadata unavailable");
    o->type=numeric_type(t);
    if(!o->type && text_type(t) && H5Tget_size(t)==1)o->type=NC_CHAR;
    REQUIRE(o->type,"unsupported dataset type");
    o->rank=H5Sget_simple_extent_ndims(s);
    REQUIRE(H5Sget_simple_extent_type(s)!=H5S_NULL && o->rank>=0 && o->rank<=MAX_RANK,"unsupported dataset rank");
    REQUIRE(H5Sget_simple_extent_dims(s,o->shape,NULL)>=0,"dataset shape unavailable");
    REQUIRE(layout(d,H5Tget_size(t),o->rank)==0 && attributes(d,o)==0,"dataset profile inspection failed");
    if(H5Aexists(d,"CLASS")>0) {
        char value[128];REQUIRE(read_text(d,"CLASS",value,sizeof(value))==0 && !strcmp(value,"DIMENSION_SCALE"),"unknown dataset class");
        o->scale=1;REQUIRE(o->rank==1,"dimension scale must be one-dimensional");
    }
    H5Sclose(s);H5Tclose(t);H5Dclose(d);return 0;
}

static int target(hid_t owner,const hobj_ref_t *ref) {
    hid_t d=H5Rdereference2(owner,H5P_DEFAULT,H5R_OBJECT,ref);
    REQUIRE(d>=0,"dangling dimension reference");
    H5O_info_t info;int ok=H5Oget_info(d,&info)>=0 && info.type==H5O_TYPE_DATASET && info.fileno==file_number;
    H5Dclose(d);REQUIRE(ok,"dimension reference leaves inspected file");
    for(int i=0;i<count;i++)if(objects[i].address==info.addr)return i;
    return reject("dimension reference target not in complete index");
}

static int references(hid_t file,int index) {
    Object *o=&objects[index];hid_t d=H5Dopen2(file,o->name,H5P_DEFAULT);REQUIRE(d>=0,"dataset cannot reopen");
    if(H5Aexists(d,"DIMENSION_LIST")>0) {
        REQUIRE(!o->scale,"dimension scale has conflicting dimension list");
        hid_t a=H5Aopen(d,"DIMENSION_LIST",H5P_DEFAULT),t=H5Aget_type(a),s=H5Aget_space(a);
        hvl_t refs[MAX_RANK]={0};REQUIRE(H5Aread(a,t,refs)>=0,"dimension references cannot be read");
        for(int i=0;i<o->rank;i++) {
            REQUIRE(refs[i].len==1 && refs[i].p,"one exact scale is required for every dimension");
            int dest=target(d,refs[i].p);REQUIRE(dest>=0 && objects[dest].scale && objects[dest].shape[0]==o->shape[i],"dimension scale or length mismatch");
            o->targets[i]=dest;
        }
        REQUIRE(H5Dvlen_reclaim(t,s,H5P_DEFAULT,refs)>=0,"dimension reference reclamation failed");
        H5Sclose(s);H5Tclose(t);H5Aclose(a);
    } else if(o->scale) o->targets[0]=index;
    else REQUIRE(o->rank==0,"array lacks complete dimension references");
    if(H5Aexists(d,"REFERENCE_LIST")>0) {
        REQUIRE(o->scale,"reverse references require a dimension scale");
        hid_t a=H5Aopen(d,"REFERENCE_LIST",H5P_DEFAULT);int rank;hssize_t n;
        REQUIRE(attribute_shape(a,&rank,&n)==0,"reverse reference shape unavailable");
        hid_t memory=H5Tcreate(H5T_COMPOUND,sizeof(Backref));
        REQUIRE(memory>=0 && H5Tinsert(memory,"dataset",HOFFSET(Backref,dataset),H5T_STD_REF_OBJ)>=0 &&
            H5Tinsert(memory,"dimension",HOFFSET(Backref,dimension),H5T_NATIVE_INT64)>=0,"reference memory layout failed");
        Backref *refs=calloc((size_t)n,sizeof(*refs));REQUIRE(refs,"reference allocation failed");
        REQUIRE(H5Aread(a,memory,refs)>=0,"reverse references cannot be read");
        for(hssize_t i=0;i<n;i++) {
            int dest=target(d,&refs[i].dataset);int64_t axis=refs[i].dimension;
            REQUIRE(dest>=0 && axis>=0 && axis<objects[dest].rank && !o->backrefs[dest][axis],"invalid or duplicate reverse reference");
            o->backrefs[dest][axis]=1;
        }
        free(refs);H5Tclose(memory);H5Aclose(a);
    }
    H5Dclose(d);return 0;
}

static int binding(hid_t file,int nc) {
    int nvars,ndims,ngroups,ntypes,ids[MAX_OBJECTS],dims[MAX_OBJECTS];
    REQUIRE(nc_inq_grps(nc,&ngroups,NULL)==NC_NOERR && ngroups==0 &&
        nc_inq_typeids(nc,&ntypes,NULL)==NC_NOERR && ntypes==0,"unsupported NetCDF groups or declared types");
    REQUIRE(nc_inq_varids(nc,&nvars,NULL)==NC_NOERR && nvars<=256 &&
        nc_inq_dimids(nc,&ndims,NULL,0)==NC_NOERR && ndims<=256,"NetCDF index exceeds profile");
    REQUIRE(nc_inq_varids(nc,NULL,ids)==NC_NOERR && nc_inq_dimids(nc,NULL,dims,0)==NC_NOERR,"NetCDF index unavailable");
    for(int i=0;i<nvars;i++) {
        char name[NC_MAX_NAME+1];nc_type type;int rank,axes[MAX_RANK];
        REQUIRE(nc_inq_var(nc,ids[i],name,&type,&rank,NULL,NULL)==NC_NOERR && rank<=MAX_RANK,"NetCDF variable metadata invalid");
        REQUIRE(nc_inq_vardimid(nc,ids[i],axes)==NC_NOERR,"NetCDF variable dimensions unavailable");
        int found=-1;for(int j=0;j<count;j++)if(!strcmp(objects[j].name,name)){found=j;break;}
        REQUIRE(found>=0,"NetCDF variable has no inspected dataset");Object *o=&objects[found];o->variable=ids[i];
        REQUIRE(o->type==type && o->rank==rank,"NetCDF type or rank differs from storage");
        for(int axis=0;axis<rank;axis++) {
            char dimname[NC_MAX_NAME+1];size_t length;
            REQUIRE(nc_inq_dim(nc,axes[axis],dimname,&length)==NC_NOERR && length==o->shape[axis],"NetCDF dimension size mismatch");
            int dest=o->targets[axis];REQUIRE(dest>=0 && objects[dest].scale && !strcmp(objects[dest].name,dimname),"NetCDF dimension binding mismatch");
        }
    }
    for(int i=0;i<ndims;i++) {
        char name[NC_MAX_NAME+1];size_t length;int found=-1;
        REQUIRE(nc_inq_dim(nc,dims[i],name,&length)==NC_NOERR,"NetCDF dimension unavailable");
        for(int j=0;j<count;j++)if(!strcmp(objects[j].name,name)){found=j;break;}
        REQUIRE(found>=0 && objects[found].scale && objects[found].shape[0]==length,"NetCDF dimension has no complete scale");
        hid_t d=H5Dopen2(file,name,H5P_DEFAULT),a=H5Aopen(d,"_Netcdf4Dimid",H5P_DEFAULT);int id=-1;
        REQUIRE(d>=0 && a>=0,"dimension identity declaration missing");
        hid_t t=H5Aget_type(a);int rank;hssize_t n;
        REQUIRE(t>=0 && numeric_type(t)==NC_INT && attribute_shape(a,&rank,&n)==0 && rank==0 && n==1 &&
            H5Aread(a,H5T_NATIVE_INT,&id)>=0 && id==dims[i],"dimension identity declaration mismatch");
        H5Tclose(t);H5Aclose(a);H5Dclose(d);
    }
    for(int i=0;i<count;i++) {
        Object *o=&objects[i];
        if(o->variable<0) {
            char value[256];hid_t d=H5Dopen2(file,o->name,H5P_DEFAULT);
            REQUIRE(o->scale && read_text(d,"NAME",value,sizeof(value))==0 &&
                !strncmp(value,"This is a netCDF dimension but not a netCDF variable.",sizeof("This is a netCDF dimension but not a netCDF variable.")-1),"unexplained HDF5 dataset");
            H5Dclose(d);
        }
        for(int j=0;j<count;j++)for(int axis=0;axis<objects[j].rank;axis++) {
            int forward=!objects[j].scale && objects[j].targets[axis]==i;
            REQUIRE((int)o->backrefs[j][axis]==forward,"dimension references are not reciprocal");
        }
    }
    return 0;
}

static int inspect(const char *path) {
    hid_t access=H5Pcreate(H5P_FILE_ACCESS);REQUIRE(access>=0 && H5Pset_fapl_sec2(access)>=0,"native single-file driver unavailable");
    hid_t file=H5Fopen(path,H5F_ACC_RDONLY,access);H5Pclose(access);REQUIRE(file>=0,"HDF5 file cannot be opened");
    H5O_info_t info;REQUIRE(H5Oget_info(file,&info)>=0,"root identity unavailable");file_number=info.fileno;
    hid_t creation=H5Fget_create_plist(file);hsize_t userblock;
    REQUIRE(creation>=0 && H5Pget_userblock(creation,&userblock)>=0 && userblock==0,"HDF5 user block is outside profile");H5Pclose(creation);
    REQUIRE(attributes(file,NULL)==0,"root attributes failed inspection");
    char provenance[1024];REQUIRE(read_text(file,"_NCProperties",provenance,sizeof(provenance))==0 && provenance[0],"NetCDF provenance declaration missing");
    REQUIRE(H5Literate(file,H5_INDEX_NAME,H5_ITER_INC,NULL,collect,NULL)>=0,"root link inspection failed");
    for(int i=0;i<count;i++)REQUIRE(references(file,i)==0,"dimension reference inspection failed");
    int nc,format;
    REQUIRE(nc_open(path,NC_NOWRITE,&nc)==NC_NOERR,"NetCDF metadata cannot be opened");
    REQUIRE(nc_inq_format(nc,&format)==NC_NOERR && (format==NC_FORMAT_NETCDF4 || format==NC_FORMAT_NETCDF4_CLASSIC),"not a NetCDF-4 container");
    int result=binding(file,nc);nc_close(nc);H5Fclose(file);return result;
}

int main(int argc,char **argv) {
    if(argc!=2)return 2;
    struct rlimit cpu={30,30};if(setrlimit(RLIMIT_CPU,&cpu)!=0){perror("NetCDF-4 CPU limit");return 2;}
    unsetenv("HDF5_VOL_CONNECTOR");unsetenv("HDF5_DRIVER");unsetenv("HDF5_PLUGIN_PATH");
    setenv("HDF5_PLUGIN_PRELOAD","::",1);
    setenv("NCRCENV_IGNORE","1",1);
    /* Rejections may occur inside a traversal with open handles. The process is
     * read-only and short-lived; never run HDF5 global teardown on partial state. */
    if(H5dont_atexit()<0)return 2;
    H5Eset_auto2(H5E_DEFAULT,NULL,NULL);
    if(H5PLset_loading_state(0)<0 || inspect(argv[1])<0) {
        fprintf(stderr,"NetCDF-4 profile rejected: %s\n",failure?failure:"native inspection failed");return 1;
    }
    puts("{\"schema\":\"addp.netcdf4.composition/v1\",\"profile\":\"root_numeric_v1\",\"complete\":true}");
    return 0;
}
